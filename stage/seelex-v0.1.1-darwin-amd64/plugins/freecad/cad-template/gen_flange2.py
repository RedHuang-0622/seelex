import FreeCAD as App
import Part, math

doc = App.newDocument('FlangeCoupling')

OD, ID, THK = 120, 50, 20
PCD, BOLT_D = 90, 12
CB_D, CB_H = 20, 2
KEY_W, KEY_D = 14, 5.5
RIB_T, RIB_H = 8, 15
FIL_IN, FIL_OUT = 2, 3
RHO = 7.85e-6
BOLT_COUNT = 6

# 1. Flange ring
outer = doc.addObject('Part::Cylinder', 'Outer')
outer.Radius = OD/2
outer.Height = THK
inner = doc.addObject('Part::Cylinder', 'InnerCut')
inner.Radius = ID/2
inner.Height = THK
flange = doc.addObject('Part::Cut', 'FlangeRing')
flange.Base = outer
flange.Tool = inner
doc.recompute()
print('1. Flange ring OK')

# 2. Bolt holes + counterbores
tools = []
for i in range(BOLT_COUNT):
    a = 2 * math.pi * i / BOLT_COUNT
    x, y = PCD/2 * math.cos(a), PCD/2 * math.sin(a)
    bh = doc.addObject('Part::Cylinder', f'BH_{i}')
    bh.Radius = BOLT_D/2
    bh.Height = THK + 2
    bh.Placement.Base = App.Vector(x, y, -1)
    tools.append(bh)
    cbt = doc.addObject('Part::Cylinder', f'CB_T_{i}')
    cbt.Radius = CB_D/2
    cbt.Height = CB_H + 0.1
    cbt.Placement.Base = App.Vector(x, y, THK - CB_H)
    tools.append(cbt)
    cbb = doc.addObject('Part::Cylinder', f'CB_B_{i}')
    cbb.Radius = CB_D/2
    cbb.Height = CB_H + 0.1
    cbb.Placement.Base = App.Vector(x, y, -0.1)
    tools.append(cbb)

tf = doc.addObject('Part::MultiFuse', 'HoleTools')
tf.Shapes = tools
doc.recompute()
flange_h = doc.addObject('Part::Cut', 'FlangeHoles')
flange_h.Base = flange
flange_h.Tool = tf
doc.recompute()
print('2. Holes OK')

# 3. Keyway
key = doc.addObject('Part::Box', 'KeywayCut')
key.Length = KEY_W
key.Width = THK + 4
key.Height = KEY_D + 2
key.Placement.Base = App.Vector(-KEY_W/2, ID/2 - 1, -2)
flange_k = doc.addObject('Part::Cut', 'FlangeKeyed')
flange_k.Base = flange_h
flange_k.Tool = key
doc.recompute()
print('3. Keyway OK')

# 4. Ribs
ribs = []
mid_r = (ID/2 + OD/2) / 2
for i in range(6):
    a = 2 * math.pi * i / 6 + math.radians(30)
    rib = doc.addObject('Part::Box', f'Rib_{i}')
    rib.Length = RIB_T
    rib.Width = OD/2 - ID/2
    rib.Height = RIB_H
    rot = App.Rotation(App.Vector(0,0,1), math.degrees(a))
    cl = App.Vector(RIB_T/2, (OD/2 - ID/2)/2, RIB_H/2)
    cr = rot.multVec(cl)
    tgt = App.Vector(mid_r * math.cos(a), mid_r * math.sin(a), RIB_H/2)
    base = tgt - cr
    rib.Placement = App.Placement(base, rot)
    ribs.append(rib)

rf = doc.addObject('Part::MultiFuse', 'AllRibs')
rf.Shapes = ribs
doc.recompute()
flange_r = doc.addObject('Part::Fuse', 'FlangeRibbed')
flange_r.Base = flange_k
flange_r.Tool = rf
doc.recompute()
print('4. Ribs OK')

# 5. Report
doc.recompute()
try:
    vol = flange_r.Shape.Volume
    mass = vol * RHO
    print('Volume: {:.1f} mm3'.format(vol))
    print('Mass: {:.3f} kg'.format(mass))
except Exception as e:
    print('Vol err: {}'.format(e))

flange_r.Label = 'FlangeCoupling'
print('=== DONE ===')
